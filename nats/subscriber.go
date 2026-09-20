package nats

import (
	"fmt"
	"sync"

	mq "github.com/SilentQianyi/mq"
	natsLib "github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// natsSubscription NATS Core 订阅实现
type natsSubscription struct {
	conn         *natsLib.Conn
	subject      string
	handler      func(msg mq.Message)
	logger       *zap.Logger
	subscription *natsLib.Subscription
	running      bool
	mu           sync.Mutex
}

func (s *natsSubscription) start() error {
	sub, err := s.conn.Subscribe(s.subject, func(msg *natsLib.Msg) {
		s.handler(&natsMessage{raw: msg})
	})
	if err != nil {
		return fmt.Errorf("subscribe error: %w", err)
	}

	s.subscription = sub
	s.running = true
	s.logger.Info("NATS subscription started", zap.String("subject", s.subject))
	return nil
}

func (s *natsSubscription) Unsubscribe() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.subscription != nil {
		err := s.subscription.Unsubscribe()
		s.running = false
		return err
	}
	return nil
}

func (s *natsSubscription) IsActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// natsQueueSubscription NATS JetStream 队列订阅实现
type natsQueueSubscription struct {
	sub        *natsLib.Subscription
	pool       *mq.WorkerPool
	streamName string
	logger     *zap.Logger
	running    bool
	mu         sync.Mutex
	ctx        chan struct{}
}

func (s *natsQueueSubscription) start() {
	s.ctx = make(chan struct{})
	s.running = true

	go s.dispatcher()
	s.logger.Info("NATS queue subscription started", zap.String("stream", s.streamName))
}

func (s *natsQueueSubscription) dispatcher() {
	for {
		select {
		case <-s.ctx:
			s.logger.Info("Queue dispatcher stopped", zap.String("stream", s.streamName))
			return
		default:
			msgs, err := s.sub.Fetch(1, natsLib.MaxWait(100))
			if err != nil {
				if err == natsLib.ErrTimeout {
					continue
				}
				select {
				case <-s.ctx:
					return
				default:
					s.logger.Error("Fetch failed", zap.String("stream", s.streamName), zap.Error(err))
					continue
				}
			}

			for _, msg := range msgs {
				s.pool.Submit(&jetStreamMessage{raw: msg})
			}
		}
	}
}

func (s *natsQueueSubscription) Unsubscribe() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		close(s.ctx)
		s.pool.Stop()
		s.running = false
		return s.sub.Unsubscribe()
	}
	return nil
}

func (s *natsQueueSubscription) IsActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
