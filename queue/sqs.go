/*
|--------------------------------------------------------------------------
| AWS SQS Queue Adapter
|--------------------------------------------------------------------------
|
| Uses Amazon SQS for job persistence. Set AWS_REGION, AWS_ACCESS_KEY_ID,
| AWS_SECRET_ACCESS_KEY (or use IAM). Queue URL from SQS_QUEUE_URL.
|
*/

package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)


// SQSAdapter uses AWS SQS for job storage.
type SQSAdapter struct {
	client    *sqs.Client
	queueURL  string
	queueName string // fallback for GetQueueUrl
}

// NewSQSAdapter creates an SQS adapter. queueURL is the full SQS queue URL.
func NewSQSAdapter(ctx context.Context, queueURL string) (*SQSAdapter, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &SQSAdapter{
		client:   sqs.NewFromConfig(cfg),
		queueURL: queueURL,
	}, nil
}

// NewSQSAdapterFromConfig creates adapter with custom config.
func NewSQSAdapterFromConfig(client *sqs.Client, queueURL string) *SQSAdapter {
	return &SQSAdapter{client: client, queueURL: queueURL}
}

// Push adds a job to the queue.
func (s *SQSAdapter) Push(ctx context.Context, payload *JobPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	attrs := map[string]types.MessageAttributeValue{
		"JobName": {DataType: aws.String("String"), StringValue: aws.String(payload.JobName)},
		"Queue":   {DataType: aws.String("String"), StringValue: aws.String(payload.Queue)},
	}
	req := &sqs.SendMessageInput{
		QueueUrl:       aws.String(s.queueURL),
		MessageBody:    aws.String(string(data)),
		MessageAttributes: attrs,
	}
	if payload.Delay > 0 {
		secs := int32(payload.Delay.Seconds())
		if secs > 900 {
			secs = 900 // SQS max 15 min
		}
		req.DelaySeconds = secs
	}
	_, err = s.client.SendMessage(ctx, req)
	return err
}

// Pop blocks until a job is available (long polling).
func (s *SQSAdapter) Pop(ctx context.Context, queue string) (*JobPayload, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			out, err := s.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
				QueueUrl:            aws.String(s.queueURL),
				MaxNumberOfMessages: 1,
				WaitTimeSeconds:    20,
				MessageAttributeNames: []string{"All"},
			})
			if err != nil {
				return nil, err
			}
			if len(out.Messages) == 0 {
				continue
			}
			msg := out.Messages[0]
			var p JobPayload
			if err := json.Unmarshal([]byte(aws.ToString(msg.Body)), &p); err != nil {
				_, _ = s.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
					QueueUrl:      aws.String(s.queueURL),
					ReceiptHandle: msg.ReceiptHandle,
				})
				continue
			}
			if p.Meta == nil {
				p.Meta = make(map[string]interface{})
			}
			p.Meta["sqs_receipt_handle"] = aws.ToString(msg.ReceiptHandle)
			return &p, nil
		}
	}
}

// Len returns approximate message count (SQS ApproximateNumberOfMessages).
func (s *SQSAdapter) Len(ctx context.Context, queue string) (int, error) {
	out, err := s.client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(s.queueURL),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameApproximateNumberOfMessages},
	})
	if err != nil {
		return 0, err
	}
	if v, ok := out.Attributes[string(types.QueueAttributeNameApproximateNumberOfMessages)]; ok {
		var n int
		_, _ = fmt.Sscanf(v, "%d", &n)
		return n, nil
	}
	return 0, nil
}

// Complete deletes the SQS message after successful processing.
func (s *SQSAdapter) Complete(ctx context.Context, payload *JobPayload) error {
	rh, _ := payload.Meta["sqs_receipt_handle"].(string)
	if rh == "" {
		return nil
	}
	_, err := s.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(s.queueURL),
		ReceiptHandle: aws.String(rh),
	})
	return err
}

var _ CompletableAdapter = (*SQSAdapter)(nil)
