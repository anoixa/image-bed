package config

import (
	"sync"

	"github.com/anoixa/image-bed/database/models"
)

// EventType 事件类型
type EventType string

const (
	EventConfigCreated EventType = "config:created"
	EventConfigUpdated EventType = "config:updated"
	EventConfigDeleted EventType = "config:deleted"
)

// Event 配置变更事件
type Event struct {
	Type   EventType
	Config *models.SystemConfig
}

// EventHandler 事件处理器函数
type EventHandler func(*Event)

// EventBus 简单的事件总线
type EventBus struct {
	subscribers map[EventType][]EventHandler
	mu          sync.RWMutex
}

// NewEventBus 创建新的事件总线
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[EventType][]EventHandler),
	}
}

// Subscribe 订阅事件
func (eb *EventBus) Subscribe(eventType EventType, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.subscribers[eventType] = append(eb.subscribers[eventType], handler)
}

// Publish 发布事件
func (eb *EventBus) Publish(eventType EventType, config interface{}) {
	eb.mu.RLock()
	handlers := eb.subscribers[eventType]
	eb.mu.RUnlock()

	cfg, _ := config.(*models.SystemConfig)
	event := &Event{
		Type:   eventType,
		Config: cfg,
	}

	for _, handler := range handlers {
		go handler(event)
	}
}
