# AI SDK Deep Dive - Nimbus

The `plugins/ai` package is a comprehensive AI orchestration layer for Go, integrated directly into the Nimbus framework.

## Architecture

Inspired by Vercel AI SDK and LangChain, it provides unified abstractions for:
-   **Text Generation & Streaming**
-   **Structured Output (Extraction)**
-   **Agents & Reasoning Loops**
-   **Function Calling (Tools)**
-   **Embeddings & Vector Stores**
-   **RAG Engine**
-   **Prompt Templates & Chains**
-   **Workflows (Pipelines)**
-   **Image & Video Generation**
-   **Observability & Cost Tracking**

## Core Components

### Clients & Providers
The `ai.Client` handles communication with multiple providers (OpenAI, Anthropic, Gemini, Ollama, etc.) through a unified interface. Providers support custom base URLs (`OPENAI_BASE_URL`, `ANTHROPIC_BASE_URL` / `ANTHROPIC_API_URL`) and automatic exponential backoff retries on transient network/gateway errors (`502`, `503`, `504`, `429`) as well as dropped connections/unexpected EOFs.

### Dedicated Media Routing
Image and video generation can be routed to separate providers, endpoints, and models via environment variables or `ai.Config`:
- `AI_IMAGE_PROVIDER`, `AI_IMAGE_MODEL`, `AI_IMAGE_API_KEY`, `AI_IMAGE_BASE_URL`
- `AI_VIDEO_PROVIDER`, `AI_VIDEO_MODEL`, `AI_VIDEO_API_KEY`, `AI_VIDEO_BASE_URL`
When unset, media builders fall back seamlessly to the primary text provider.

### Fallback Model
`AI_FALLBACK_MODEL` names the model to retry on when the main one fails (out of credit, overloaded). Give it its own account with `AI_FALLBACK_PROVIDER`, `AI_FALLBACK_API_KEY` and `AI_FALLBACK_BASE_URL`: any request whose model is the fallback model (`ai.WithModel(os.Getenv("AI_FALLBACK_MODEL"))`) is then routed there. Base URLs may be given with or without a trailing `/chat/completions`.

### Changing Models at Runtime
`ai.Reload()` rebuilds the global client from the current environment. To switch models, keys or URLs without a restart, `os.Setenv` the `AI_*` variables and call `ai.Reload()`. On error the previous client is kept.

### Streaming Responses
`c.SSEStream` lifts the server's read/write deadlines for that response, so long streamed agent replies are not cut off by `SERVER_WRITE_TIMEOUT`. While the handler is quiet it sends a `: keep-alive` comment every `http.SSEKeepAlive` (15s) so proxies such as Cloudflare do not drop the idle connection.

### Agents
Agents combine instructions, tools, and memory to perform complex tasks autonomously.
```go
agent := ai.NewAgent("You are a researcher").WithTools("search", "writer")
resp, _ := agent.Prompt(ctx, "Research Go concurrency")
```

### Tools
Register Go functions as tools that agents can call. Use the `ai.Tool` struct for registration.

### Vector Stores
Backends for storage and retrieval of embeddings:
-   **In-Memory**: For development.
-   **pgvector**: PostgreSQL integration.
-   **Qdrant / Pinecone**: Dedicated vector databases.

### RAG Engine
Combines vector stores and text generation for Retrieval-Augmented Generation.
```go
rag := ai.NewRAG(store).TopK(5)
answer, _ := rag.Ask(ctx, "What is goroutine?")
```

## Structured Output

Extract typed Go structs from strings using `ai.Extract[T](ctx, text)`.

## Observability

Built-in support for OpenTelemetry tracing and cost tracking with budget alerts.

## Best Practices

1.  **Use Streaming**: Prefer `Stream()` for long responses to improve user experience.
2.  **Granular Tools**: Define specialized tools rather than general-purpose ones.
3.  **Guardrails**: Apply `ai.Guardrails` to validate and filter the AI's output.
4.  **Cost Monitoring**: Enable cost tracking in production to stay within budget.

### Website Studio application architecture

In `nimbus-starter`, Vani generation uses a persisted `sites.Plan` with `PlanPage.Scenes`
(narrative beat, visual, layout, motion, evidence, transition). `generationGuidance`
provides one bounded creative contract rather than keyword-selected skill bundles.
The static HTML runtime uses locally bundled Motion JavaScript, not React JSX.

Studio chat uses the same concise instructions in lean and cached modes. Per-turn
results remain stable for prefix reuse. `memory.RecallSite` scopes recalled facts to
sessions owned by the user for the current website; `sites.ProjectMemory` carries
bounded design intent, not claims about current file contents. Context fingerprints
include changed profile and project intent. Compaction sees unsummarized history
before the replay cap and accounts for tool payloads. Cache counts are telemetry,
not evidence that an unreported cache is unavailable. Full application notes and
opt-in evaluation commands: `nimbus-starter/docs/engineering/vani-architecture.md`.

Studio generation progress now persists `SiteJob.Written` independently of its
planned page list. Poll by job ID within the authorized workspace. A null job is
never completion; only a done job with recorded page output and a commit permits
success. Generation chat summaries are server-authored from observed job state.
The existing SiteJob auto-migration adds the Written column on deployment.

Studio session responses include each message's persisted ID for incremental DOM
reconciliation. Running-turn polling must preserve unchanged message nodes and
scroll position; history temporarily detaches nodes to preserve event handlers.


## Strobe motion direction (Cloud application)

Strobe routes through `/api/v1/ai/generate` with `project_context.app = "strobe"`.
Its implementation lives in `nimbus-starter/app/controllers/strobe_*.go`, not in
framework video synthesis. `task: "direct"` runs treatment → film → at most one
repair; reference images and their instructions accompany every pass. Invalid
film JSON gets one repair before the explicit template fallback. The repair
uses the complete validated cut, without truncating JSON. Unresolved structural
notes and fallback use are returned in `hints`.

Scenes have no global timestamps. Design layers have scene-local seconds for
`at`, `until`, `clicks`, morphs and keyframes, even within groups. The renderer
preserves animation endpoints and narration across music-aligned cuts. `seconds`
is a target, not an exact export guarantee; action timing, narration and music
can extend it. The direct review measures explicit-duration cuts against this
budget without imposing scene, layer or camera-count quotas.
Script direction budgets reveal/action/hold and shares a type/grid/colour system.
Custom choreography uses drawn scenes; template pieces, depth and glow are
optional, rather than automatic visual substitutions.

Supported: path morphs, solid/device layers, built-in WebGL worlds and materials,
flat animated characters and painted stills. Unsupported: synthesized filmed
footage, imported arbitrary 3D assets, rigged character performances. Product
captures reach the initial director as text/hotspot metadata. Direction must
distinguish marketing/sign-in pages from working UI. Strobe now follows the
first cut with a browser-rendered proof and selected-model visual critic.

`task: "critique"` requires rendered frame sheets (`reference_images`,
`seeing: "render"`), the current `film`, and JSON-string `measurements`.
It returns `verdict: "pass" | "revise" | "unverified"`, `hints` and a whole
repaired `film` for revisions. `preserve_script: true` compares copy per scene
and scene order, rejecting changed/dropped words while allowing layout changes.
The Strobe application allows at most two repairs and three critic calls, using
the same model as generation. Each repair is rendered before acceptance; a
candidate with more measured errors is rejected. Only an explicit pass with
zero measured errors becomes `production.status: "reviewed"`. Browser/provider
failure or old Cloud protocol preserves the draft as unverified.

The proof uses the shared engine, waits for fonts/assets, measures text bounds,
overlaps, later opaque rectangles covering text, readable holds, timing and runtime, and samples three moments per
resolved shot. This is sampled visual review, not every-frame or audio QA.
Reports carry a stable spec signature; edits invalidate review in the editor.
The quality action reviews the current unsaved snapshot with the selected model.
No silent editor rewrite runs when generation already supplied review metadata.

Strobe's `/proof/{key}` and asset routes use opaque 90-second capabilities,
immutable snapshots and snapshot-scoped asset lists. `STROBE_RENDER_URL` must
point to the same application instance reachable by its Chromium. Proofs share
the bounded capture pool. The migration
`20261014000001_add_strobe_generation_state` adds the video's `generation` text
column. A 10-second heartbeat persists progress without provider credentials;
other workers can read it. Unfinished jobs without a heartbeat for 45 seconds
report interruption and keep saved drafts/captures. Interrupted calls do not
automatically resume. Scoped draft updates cannot resurrect deleted videos.


Strobe model selection: the proxy exposes `base` for an explicit base/ChatGPT
choice and `strobe-default` for automatic direction. Empty legacy request IDs
remain automatic. An admin's explicit `base` pick becomes a non-nil Picked,
which prevents Opus override and silent fallback. Other Vani pickers retain
`aimodels.Choices()` IDs. Response labels use the successful provider's model.
The editor waits for remembered model selection before automatic first-cut review.
A failed or stale model-list response cannot reset an explicit remembered
selection to automatic; unknown saved IDs are validated by the server and
shown as unavailable when the picker has a refreshed list.

`task: "remake"` accepts the existing `film` and can remake graphics-only or
script films without captured screens; references/current-render sheets remain
optional. A legacy direct request with a current film and no screens is routed
to remake. Text-box `x` remains its centre for every alignment: left-aligned
`x:50,w:80` starts at x=10, whereas `x:10,w:80` clips off-frame. Structural
review checks these off-frame anchors.


### Strobe original artwork and repeating motion (2026-10-15)

The director's ARTWORK plan describes original vector paths/groups or generated plates, shared style and independently moving subjects. Cloud validates these fields, rendered by Strobe's shared preview/export engine:

- `anchorX` / `anchorY`: 0–100 percent of the explicit layer box, default 50. x/y remain the resting centre. 50/100 is bottom-centre.
- `loop: {start: 0, end: 2, mode: "cycle"}` or `"pingpong"`: scene-local keyframe/path-morph time remapping; bounds 0–60 seconds and end > start. Cycle endpoints must match (or be equivalent complete rotations). Entrances and underlying footage remain one-shot.
- `tile: {axis: "x", span: 100, speed: -25, mirror: true}`: arbitrary vector groups/images scroll as tiled artwork. Axis x/y; span 10–200 percent of the FRAME AXIS; speed -200–200 percent/second. Mirror alternates copies and doubles the repeat period: 2*100/25 = 8 seconds. Keep foreground untiled and clip group artwork to its tile box. Rendering bounds copy count.
- Image `prompt` plus `cutout: true` requests transparent isolated artwork through the existing picture pipeline, with image allowance/provider checks. It is a still, not a rig. Use matching explicit style/palette/lighting in separate plate prompts.
- `hold` stepped easing, `hidden` / `locked`, `via: "dissolve"` and `transitionDuration` 0.1–2 seconds survive Cloud validation.

Upgrade Cloud and Strobe together. Whole-scene seamlessness also requires periodic camera/background/effects and matching boundaries; schema support or still-frame critique does not establish temporal quality or artwork quality.


### Strobe articulated 2D artwork (2026-10-16)

Cloud now validates design layer `kind: "rig"`, with `rig: {period, phase, joints, chains, parts}`. Coordinates are in the rig's own 100×100 box; equal w/h avoids distortion. Period 0.2–60 seconds; phase is cycles. Up to 32 unique joints `{id,x,y,rot?,anim?:{x,y},orbit?:{rx,ry,phase,turns}}`; rotation is fixed or keys, keys are cycle-local seconds, orbit phase is degrees and turns is a signed integer (-12..12). Up to eight independent analytic two-link chains `{root,mid,end,target,upper,lower,bend:1|-1}`; all references must exist, lengths positive, each solved mid/end unique. Endpoints outside reach are clamped without stretching and reported by Strobe quality review.

Up to 48 back-to-front parts `{id,joint,tip?,stretch?,layer}`. Part offsets default to zero at the attachment; tip points local +X along a segment; stretch fits width between joints. Original path/image/ellipse/rect/group artwork consumes the scene's 60-layer budget. Unsupported kinds/nested rigs are rejected; one-shot entrance/idle/tiling/repeat fields are stripped from parts. A shared deterministic solver drives preview, seek, proof and export; changing cadence retimes authored keys. Quality review checks entire-cycle reachability, missing attachments/artwork and endpoint continuity. Rigs and scrolling tiles count as subject motion in Cloud review.

Editor Elements → Character rig creates editable vector cyclist parts; inspector controls performance, joints, constraints, vector paths and individual generated/uploaded cutouts. Generated images receive shared art direction/palette, identical prompt/shape/cutout requests reuse an asset before the four-unique-image allowance, rig parts are traversed for painting and asset loading, and cutouts no longer receive an opaque backing. A whole character image is not automatically segmented or rigged. No 3D skinning or photoreal performance synthesis. Upgrade Cloud and Strobe together; actual illustration quality still requires rendered inspection.


### Strobe generation reliability (2026-10-17)

Strobe checks `NIMBUS_API_KEY`/Cloud URL before creating a film job (503 on missing/invalid configuration, no empty job). Cloud `strobeAsk` bounds each provider/fallback sequence to 180 seconds; `STROBE_MODEL_TIMEOUT_SECONDS` may override within 30–300, and caller deadlines take precedence. Strobe HTTP task deadlines: script/edit/critique 4m, explore 90s, picture/retouch 210s, voice 2m, remake 7m, direct/default 10m. A shared 12m job context/watchdog covers capture through review; heartbeats cannot extend it. Progress is persisted when stages change; cancelled/failed jobs cannot be marked active by late stages. Saved drafts/captures remain available.

Create UI request budgets include reading the response body (start 20s, model/status 15s). Authentication/permission/not-found failures stop polling immediately; three transient failures pause checks with a status-only retry and library link. `/new?job=<id>` resumes polling across refresh/sign-in, never repeats POST. Server elapsed time and `hasDraft` determine progress and recovery actions. A start timeout is an uncertain outcome, not proof that no job started; check the library before resubmitting.

## Runway / Kling video provider

`runway` is built in. Keep text/image clients separate with `AI_VIDEO_PROVIDER=runway`,
`AI_VIDEO_MODEL=kling3.0_standard`, and `RUNWAY_API_KEY` (or `AI_VIDEO_API_KEY`).
Optional endpoint: `AI_VIDEO_BASE_URL` for media clients, `RUNWAY_BASE_URL` for direct
Runway clients. Config fields: `RunwayKey string`, `RunwayBaseURL string`.
`ai.RunwayKlingStandard` is `"kling3.0_standard"`. Audio defaults **off** in Nimbus
(the upstream default is on); `.Audio(true)` explicitly enables it at greater cost.
Standard sizes: `1280x720`, `720x1280`, `960x960`; duration 3–15 seconds, default 5;
prompt 2–2500 UTF-16 characters. Do not use `.FPS()` for Kling. Runway also accepts
`kling3.0_pro`, `kling3.0_4k`, `kling2.5_turbo_pro`, subject to their size/duration rules.

```go
video, err := ai.Video().Model(ai.RunwayKlingStandard).
    Prompt("A cyclist pedals through a golden meadow").
    Duration(5).Size("720x1280").Audio(false).Generate(ctx)
if err != nil { return err }
fmt.Println(video.URL, video.TaskID, video.Credits)
```

`.Generate(ctx)` waits at most 10 minutes and attempts cancellation on interruption.
A background worker should start once, persist the ID/owner, then poll the same task:

```go
client := ai.GetClient()
task, err := ai.Video().WithClient(client).Model(ai.RunwayKlingStandard).
    Prompt("A cyclist pedals through a golden meadow").Duration(5).Audio(false).Start(ctx)
if err != nil { return err }
state, err := client.GetVideoTask(ctx, task.ID)
if err != nil { return err }
fmt.Println(state.Status, state.Output)
// Explicitly cancel when appropriate:
if err := client.CancelVideoTask(ctx, task.ID); err != nil { return err }
```

New signatures: `(*VideoBuilder).Audio(bool) *VideoBuilder`,
`(*VideoBuilder).Start(context.Context) (*VideoTask, error)`,
`(*Client).GetVideoTask(context.Context, string) (*VideoTask, error)`,
`(*Client).CancelVideoTask(context.Context, string) error`.
`AsyncVideoProvider` requires `StartVideo`, `GetVideoTask`, `CancelVideoTask`.
`VideoTask`: ID/Status/Failure/FailureCode strings, Output []string,
Progress/EstimatedCredits float64. `VideoResponse` adds TaskID string and Credits float64;
`VideoRequest` adds Audio *bool. Statuses include PENDING, THROTTLED, RUNNING, SUCCEEDED,
FAILED, CANCELED. Do not automatically retry task creation (it is billable). Copy successful
output into application storage; provider URLs expire. The generic VideoPipeline's 2-second
draft setting and default 1080p output are incompatible with Standard: explicitly use
DraftMode(false) and a supported OutputFormat. Video synthesis produces rendered footage,
not independently editable character parts.

Nimbus Cloud Strobe tasks: `brief` expands human creative direction through the selected
text model; `video-check`, `video-start`, `video-status`, `video-cancel` use a separate Runway
client. Video defaults to Kling Standard with audio off. Tickets are HMAC signed, bound to
Cloud key owner + Strobe end user, and expire after 24 hours. A non-default APP_KEY is required.
Strobe charges a successful task only once using the unique provider task ID.
