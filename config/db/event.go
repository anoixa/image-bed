package config

import (
	"sync"

	"github.com/anoixa/image-bed/database/models"
)

// EventType 事件类型
type EventType string

const (
	// EventConfigCreated 配置创建事件
	EventConfigCreated EventType = "config:created"
	// EventConfigUpdated 配置更新事件
	EventConfigUpdated EventType = "config:updated"
	// EventConfigDeleted 配置删除事件
	EventConfigDeleted EventType = "config:deleted"
	// EventConfigEnabled 配置启用事件
	EventConfigEnabled EventType = "config:enabled"
	// EventConfigDisabled 配置禁用事件
	EventConfigDisabled EventType = "config:disabled"
)

// Event 事件结构
type Event struct {
	Type   EventType
	Config *models.SystemConfig
}

// EventHandler 事件处理器
type EventHandler func(event *Event)

// EventBus 事件总线
type EventBus struct {
	subscribers map[EventType][]EventHandler
	mu          sync.RWMutex
}

// NewEventBus 创建事件总线
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[EventType][]EventHandler),
	}
}

// Subscribe 订阅事件
func (eb *EventBus) Subscribe(eventType EventType, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.subscribers[eventType] == nil {
		eb.subscribers[eventType] = make([]EventHandler, 0)
	}
	eb.subscribers[eventType] = append(eb.subscribers[eventType], handler)
}

// Unsubscribe 取消订阅（简化实现：清除所有该类型的处理器）
func (eb *EventBus) Unsubscribe(eventType EventType) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	delete(eb.subscribers, eventType)
}

// Publish 发布事件
func (eb *EventBus) Publish(eventType EventType, config *models.SystemConfig) {
	eb.mu.RLock()
	handlers := eb.subscribers[eventType]
	eb.mu.RUnlock()

	if len(handlers) == 0 {
		return
	}

	event := &Event{
		Type:   eventType,
		Config: config,
	}

	// 异步执行处理器
	for _, handler := range handlers {
		go handler(event)
	}
}

// PublishSync 同步发布事件
func (eb *EventBus) PublishSync(eventType EventType, config *models.SystemConfig) {
	eb.mu.RLock()
	handlers := eb.subscribers[eventType]
	eb.mu.RUnlock()

	if len(handlers) == 0 {
		return
	}

	event := &Event{
		Type:   eventType,
		Config: config,
	}

	// 同步执行处理器
	for _, handler := range handlers {
		handler(event)
	}
}
