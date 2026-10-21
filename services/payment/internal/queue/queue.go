package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const queueName = "fingo.payment-events"

type Event struct {
	ID        int64           `json:"id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

type Broker struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	confirmed  <-chan amqp.Confirmation
}

func Connect(url string) (*Broker, error) {
	connection, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	channel, err := connection.Channel()
	if err != nil {
		connection.Close()
		return nil, fmt.Errorf("open RabbitMQ channel: %w", err)
	}
	if err := channel.Confirm(false); err != nil {
		channel.Close()
		connection.Close()
		return nil, fmt.Errorf("enable RabbitMQ publisher confirms: %w", err)
	}
	confirmed := channel.NotifyPublish(make(chan amqp.Confirmation, 1))
	if _, err := channel.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		channel.Close()
		connection.Close()
		return nil, fmt.Errorf("declare RabbitMQ queue: %w", err)
	}
	return &Broker{connection: connection, channel: channel, confirmed: confirmed}, nil
}

func (b *Broker) Publish(ctx context.Context, event Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode queue event: %w", err)
	}
	if err := b.channel.PublishWithContext(ctx, "", queueName, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    fmt.Sprint(event.ID),
		Timestamp:    time.Now().UTC(),
		Body:         body,
	}); err != nil {
		return fmt.Errorf("publish queue event: %w", err)
	}
	select {
	case confirmation, ok := <-b.confirmed:
		if !ok || !confirmation.Ack {
			return fmt.Errorf("RabbitMQ did not confirm event %d", event.ID)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *Broker) Consume(ctx context.Context, handle func(context.Context, Event) error) error {
	if err := b.channel.Qos(1, 0, false); err != nil {
		return fmt.Errorf("set RabbitMQ prefetch: %w", err)
	}
	deliveries, err := b.channel.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume RabbitMQ queue: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("RabbitMQ delivery channel closed")
			}
			var event Event
			if err := json.Unmarshal(delivery.Body, &event); err != nil {
				if nackErr := delivery.Nack(false, false); nackErr != nil {
					return fmt.Errorf("reject malformed event: %w", nackErr)
				}
				continue
			}
			if err := handle(ctx, event); err != nil {
				if nackErr := delivery.Nack(false, true); nackErr != nil {
					return fmt.Errorf("requeue failed event: %w", nackErr)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				continue
			}
			if err := delivery.Ack(false); err != nil {
				return fmt.Errorf("acknowledge event: %w", err)
			}
		}
	}
}

func (b *Broker) Close() error {
	channelErr := b.channel.Close()
	connectionErr := b.connection.Close()
	if channelErr != nil {
		return channelErr
	}
	return connectionErr
}
