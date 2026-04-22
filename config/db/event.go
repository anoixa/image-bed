package config

import (
	"sync"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils"
)

var eventBusLog = utils.ForModule("EventBus")

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
func (eb *EventBus) Publish(eventType EventType, config *models.SystemConfig) {
	eb.mu.RLock()
	handlers := append([]EventHandler(nil), eb.subscribers[eventType]...)
	eb.mu.RUnlock()

	event := &Event{
		Type:   eventType,
		Config: config,
	}

	for _, handler := range handlers {
		func(h EventHandler) {
			defer func() {
				if r := recover(); r != nil {
					eventBusLog.Errorf("Handler panicked for event %s: %v", eventType, r)
				}
			}()
			h(event)
		}(handler)
	}
}
