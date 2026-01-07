package audittrail

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"cloud.google.com/go/pubsub"
)

// Publisher sends an audit entry to an external queue (e.g., Pub/Sub, Kafka).
type Publisher interface {
	Publish(ctx context.Context, entry Entry) error
}

// PublisherFunc adapts a function to Publisher.
type PublisherFunc func(context.Context, Entry) error

func (f PublisherFunc) Publish(ctx context.Context, entry Entry) error { return f(ctx, entry) }

// Subscriber receives audit entries from an external queue (e.g., Pub/Sub, Kafka).
type Subscriber interface {
	Receive(ctx context.Context, handler func(context.Context, Entry) error) error
}

// SubscriberFunc adapts a function to Subscriber.
type SubscriberFunc func(context.Context, func(context.Context, Entry) error) error

func (f SubscriberFunc) Receive(ctx context.Context, handler func(context.Context, Entry) error) error {
	return f(ctx, handler)
}

// PubSubRecorder publishes audit entries to an external queue.
type PubSubRecorder struct {
	publisher Publisher
	now       func() time.Time
}

// NewPubSubRecorder creates a recorder that publishes entries to a queue.
func NewPubSubRecorder(publisher Publisher, now func() time.Time) (*PubSubRecorder, error) {
	if publisher == nil {
		return nil, errors.New("audittrail: publisher must not be nil")
	}
	if now == nil {
		now = time.Now
	}
	return &PubSubRecorder{
		publisher: publisher,
		now:       now,
	}, nil
}

// Record validates and publishes an entry to the queue.
func (p *PubSubRecorder) Record(ctx context.Context, entry Entry) error {
	normalized, err := normalizeEntry(entry, p.now)
	if err != nil {
		return err
	}
	return p.publisher.Publish(ctx, normalized)
}

// Consumer receives audit entries and persists them to the database.
type Consumer struct {
	audit      *AuditTrail
	subscriber Subscriber
	onError    func(error)
}

// NewConsumer wires a subscriber to a database-backed audit trail.
func NewConsumer(audit *AuditTrail, subscriber Subscriber, onError func(error)) (*Consumer, error) {
	if audit == nil {
		return nil, errors.New("audittrail: audit must not be nil")
	}
	if subscriber == nil {
		return nil, errors.New("audittrail: subscriber must not be nil")
	}
	if onError == nil {
		onError = func(err error) { log.Printf("audittrail consumer error: %v", err) }
	}
	return &Consumer{
		audit:      audit,
		subscriber: subscriber,
		onError:    onError,
	}, nil
}

// Run starts consuming entries until the subscriber stops or context is canceled.
func (c *Consumer) Run(ctx context.Context) error {
	return c.subscriber.Receive(ctx, func(ctx context.Context, entry Entry) error {
		if err := c.audit.Record(ctx, entry); err != nil {
			if c.onError != nil {
				c.onError(err)
			}
			return err
		}
		return nil
	})
}

// MarshalEntryJSON is a helper for external publishers that need JSON payloads.
func MarshalEntryJSON(entry Entry) ([]byte, error) {
	return json.Marshal(entry)
}

// ==================== GCP Pub/Sub Implementation ====================

// gcpPublisher implements Publisher interface using Google Cloud Pub/Sub.
type gcpPublisher struct {
	topic *pubsub.Topic
}

// NewGCPPublisher creates a Publisher implementation using GCP Pub/Sub.
func NewGCPPublisher(topic *pubsub.Topic) Publisher {
	return &gcpPublisher{topic: topic}
}

// Publish sends an audit entry to GCP Pub/Sub topic.
func (p *gcpPublisher) Publish(ctx context.Context, entry Entry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	result := p.topic.Publish(ctx, &pubsub.Message{Data: data})

	// Wait for publish result synchronously to properly handle errors
	if _, err := result.Get(ctx); err != nil {
		return err
	}

	return nil
}

// gcpSubscriber implements Subscriber interface using Google Cloud Pub/Sub.
type gcpSubscriber struct {
	sub *pubsub.Subscription
}

// NewGCPSubscriber creates a Subscriber implementation using GCP Pub/Sub.
func NewGCPSubscriber(sub *pubsub.Subscription) Subscriber {
	return &gcpSubscriber{sub: sub}
}

// Receive listens for messages from GCP Pub/Sub subscription.
func (s *gcpSubscriber) Receive(ctx context.Context, handler func(context.Context, Entry) error) error {
	return s.sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		var entry Entry
		if err := json.Unmarshal(msg.Data, &entry); err != nil {
			log.Printf("audittrail: failed to unmarshal pubsub message: %v, data: %s", err, string(msg.Data))
			msg.Nack()
			return
		}
		if err := handler(ctx, entry); err != nil {
			log.Printf("audittrail: handler failed for entry %s: %v", entry.ID, err)
			msg.Nack()
			return
		}
		msg.Ack()
	})
}
