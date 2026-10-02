/*
|--------------------------------------------------------------------------
| AI SDK — Reranking
|--------------------------------------------------------------------------
|
| Vector search finds what is similar; a reranker orders it by how well it
| answers the question. Retrieve generously, then keep the best:
|
|   ranked, err := ai.Rerank(ctx, "How do I reset my password?", docs, 5)
|   rag := ai.NewRAG(store).TopK(20).WithReranker(ai.GetClient(), 5)
|
| Cohere's rerank models are built in (rerank-v3.5 by default).
|
*/

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
)

// RerankResult is one document with its relevance to the query.
type RerankResult struct {
	Index    int // position in the documents passed in
	Document string
	Score    float64 // higher is more relevant (0–1 for Cohere)
}

// RerankRequest asks a provider to order documents by relevance.
type RerankRequest struct {
	Query     string
	Documents []string
	TopN      int
	Model     string
}

// RerankProvider orders documents by relevance to a query.
type RerankProvider interface {
	Rerank(ctx context.Context, req *RerankRequest) ([]RerankResult, error)
}

// Rerank orders documents by relevance to query and returns the best topN
// (all of them when topN <= 0), most relevant first.
func (c *Client) Rerank(ctx context.Context, query string, documents []string, topN int) ([]RerankResult, error) {
	rp, ok := c.provider.(RerankProvider)
	if !ok {
		return nil, fmt.Errorf("ai: provider %s cannot rerank (use Cohere)", c.provider.Name())
	}
	if len(documents) == 0 {
		return nil, nil
	}
	req := &RerankRequest{Query: query, Documents: documents, TopN: topN}
	out, err := withRetries(ctx, c.maxRetries(), func() ([]RerankResult, error) { return rp.Rerank(ctx, req) })
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if topN > 0 && len(out) > topN {
		out = out[:topN]
	}
	return out, nil
}

// Rerank uses the global client.
func Rerank(ctx context.Context, query string, documents []string, topN int) ([]RerankResult, error) {
	return GetClient().Rerank(ctx, query, documents, topN)
}

// cohereRerankURL is a var so tests can point it elsewhere.
var cohereRerankURL = "https://api.cohere.com/v2/rerank"

func (p *cohereProvider) Rerank(ctx context.Context, req *RerankRequest) ([]RerankResult, error) {
	model := req.Model
	if model == "" {
		model = "rerank-v3.5"
	}
	body := map[string]any{"model": model, "query": req.Query, "documents": req.Documents}
	if req.TopN > 0 {
		body["top_n"] = req.TopN
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cohereRerankURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, newAPIError("cohere", resp)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("cohere: parse rerank: %w", err)
	}
	results := make([]RerankResult, 0, len(out.Results))
	for _, r := range out.Results {
		if r.Index < 0 || r.Index >= len(req.Documents) {
			continue
		}
		results = append(results, RerankResult{Index: r.Index, Document: req.Documents[r.Index], Score: r.RelevanceScore})
	}
	return results, nil
}

var _ RerankProvider = (*cohereProvider)(nil)
