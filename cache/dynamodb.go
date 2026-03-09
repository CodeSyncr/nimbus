package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const dynamoPrefix = "nimbus:cache:"

// DynamoDBStore uses AWS DynamoDB for distributed caching.
// Requires a table with partition key "pk" (string) and optional "ttl" (number) for expiration.
// Enable TTL on the table for the "ttl" attribute to auto-delete expired items.
type DynamoDBStore struct {
	client    *dynamodb.Client
	tableName string
	prefix    string
}

type dynamoItem struct {
	PK  string `dynamodbav:"pk"`
	Val string `dynamodbav:"val"`
	TTL int64  `dynamodbav:"ttl,omitempty"`
}

// NewDynamoDBStore creates a DynamoDB cache store.
func NewDynamoDBStore(cfg aws.Config, tableName string) *DynamoDBStore {
	return &DynamoDBStore{
		client:    dynamodb.NewFromConfig(cfg),
		tableName: tableName,
		prefix:    dynamoPrefix,
	}
}

// NewDynamoDBStoreWithPrefix creates a DynamoDB store with a custom key prefix.
func NewDynamoDBStoreWithPrefix(cfg aws.Config, tableName, prefix string) *DynamoDBStore {
	return &DynamoDBStore{
		client:    dynamodb.NewFromConfig(cfg),
		tableName: tableName,
		prefix:    prefix,
	}
}

// Set stores a value. Values are JSON-serialized.
func (d *DynamoDBStore) Set(key string, value any, ttl time.Duration) error {
	ctx := context.Background()
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	pk := d.prefix + key
	item := dynamoItem{PK: pk, Val: string(data)}
	if ttl > 0 {
		item.TTL = time.Now().Add(ttl).Unix()
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return err
	}
	_, err = d.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item:      av,
	})
	return err
}

// Get returns the value and true if found.
func (d *DynamoDBStore) Get(key string) (any, bool) {
	ctx := context.Background()
	pk := d.prefix + key
	res, err := d.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: pk},
		},
	})
	if err != nil || res.Item == nil {
		return nil, false
	}
	var item dynamoItem
	if err := attributevalue.UnmarshalMap(res.Item, &item); err != nil {
		return nil, false
	}
	if item.TTL > 0 && time.Now().Unix() > item.TTL {
		d.Delete(key)
		return nil, false
	}
	var v any
	if err := json.Unmarshal([]byte(item.Val), &v); err != nil {
		return nil, false
	}
	return v, true
}

// Delete removes a key.
func (d *DynamoDBStore) Delete(key string) error {
	ctx := context.Background()
	pk := d.prefix + key
	_, err := d.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: pk},
		},
	})
	return err
}

// Remember returns the cached value or calls fn, stores the result, and returns it.
func (d *DynamoDBStore) Remember(key string, ttl time.Duration, fn func() (any, error)) (any, error) {
	if v, ok := d.Get(key); ok {
		return v, nil
	}
	v, err := fn()
	if err != nil {
		return nil, err
	}
	_ = d.Set(key, v, ttl)
	return v, nil
}

// EnsureTable creates the cache table if it does not exist.
// Call once during setup. Table: pk (string, partition key), val (string), ttl (number, optional for TTL).
func (d *DynamoDBStore) EnsureTable(ctx context.Context) error {
	_, err := d.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(d.tableName),
	})
	if err == nil {
		return nil // table exists
	}
	_, err = d.client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(d.tableName),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		return fmt.Errorf("cache: create dynamodb table: %w", err)
	}
	return nil
}

var _ Store = (*DynamoDBStore)(nil)
