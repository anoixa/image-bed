package config

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/anoixa/image-bed/database/models"
)

// Reloadable 可热重载接口
type Reloadable interface {
	// ReloadConfig 热重载指定配置
	ReloadConfig(configID uint) error
	// LoadFromDB 从数据库加载所有配置
	LoadFromDB() error
}

// Watcher 配置变更监听器
type Watcher struct {
	manager   *Manager
	reloaders map[string]Reloadable // key: category (storage, cache, etc.)
	stopCh    chan struct{}
	started   bool
}

// NewWatcher 创建配置监听器
func NewWatcher(manager *Manager) *Watcher {
	return &Watcher{
		manager:   manager,
		reloaders: make(map[string]Reloadable),
		stopCh:    make(chan struct{}),
	}
}

// RegisterReloader 注册可热重载组件
func (w *Watcher) RegisterReloader(category string, reloader Reloadable) {
	w.reloaders[category] = reloader
	log.Printf("[ConfigWatcher] Registered reloader for category: %s", category)
}

// Start 启动监听
func (w *Watcher) Start() {
	if w.started {
		return
	}
	w.started = true

	// 订阅配置变更事件
	w.manager.Subscribe(EventConfigCreated, w.handleConfigCreated)
	w.manager.Subscribe(EventConfigUpdated, w.handleConfigUpdated)
	w.manager.Subscribe(EventConfigDeleted, w.handleConfigDeleted)
	w.manager.Subscribe(EventConfigEnabled, w.handleConfigEnabled)
	w.manager.Subscribe(EventConfigDisabled, w.handleConfigDisabled)

	log.Println("[ConfigWatcher] Started watching config changes")
}

// Stop 停止监听
func (w *Watcher) Stop() {
	if !w.started {
		return
	}
	close(w.stopCh)
	w.started = false
	log.Println("[ConfigWatcher] Stopped watching config changes")
}

// handleConfigCreated 处理配置创建事件
func (w *Watcher) handleConfigCreated(event *Event) {
	config := event.Config
	log.Printf("[ConfigWatcher] Config created: %s (ID: %d)", config.Key, config.ID)

	// 如果配置是启用的，触发加载
	if config.IsEnabled {
		w.triggerReload(config)
	}
}

// handleConfigUpdated 处理配置更新事件
func (w *Watcher) handleConfigUpdated(event *Event) {
	config := event.Config
	log.Printf("[ConfigWatcher] Config updated: %s (ID: %d)", config.Key, config.ID)

	// 触发重载
	w.triggerReload(config)
}

// handleConfigDeleted 处理配置删除事件
func (w *Watcher) handleConfigDeleted(event *Event) {
	config := event.Config
	log.Printf("[ConfigWatcher] Config deleted: %s (ID: %d)", config.Key, config.ID)

	// 触发重载（让组件清理）
	w.triggerReload(config)
}

// handleConfigEnabled 处理配置启用事件
func (w *Watcher) handleConfigEnabled(event *Event) {
	config := event.Config
	log.Printf("[ConfigWatcher] Config enabled: %s (ID: %d)", config.Key, config.ID)

	w.triggerReload(config)
}

// handleConfigDisabled 处理配置禁用事件
func (w *Watcher) handleConfigDisabled(event *Event) {
	config := event.Config
	log.Printf("[ConfigWatcher] Config disabled: %s (ID: %d)", config.Key, config.ID)

	w.triggerReload(config)
}

// triggerReload 触发重载
func (w *Watcher) triggerReload(config *models.SystemConfig) {
	category := string(config.Category)
	reloader, ok := w.reloaders[category]
	if !ok {
		// 没有注册的 reloader，忽略
		return
	}

	// 异步重载，避免阻塞事件处理
	go func() {
		// 延迟一点时间，让批量更新完成
		time.Sleep(100 * time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		done := make(chan error, 1)
		go func() {
			done <- reloader.ReloadConfig(config.ID)
		}()

		select {
		case err := <-done:
			if err != nil {
				log.Printf("[ConfigWatcher] Failed to reload config %d: %v", config.ID, err)
			} else {
				log.Printf("[ConfigWatcher] Successfully reloaded config %d", config.ID)
			}
		case <-ctx.Done():
			log.Printf("[ConfigWatcher] Reload config %d timed out", config.ID)
		case <-w.stopCh:
			log.Printf("[ConfigWatcher] Reload config %d cancelled", config.ID)
		}
	}()
}

// LoadAll 加载所有配置到注册的组件
func (w *Watcher) LoadAll() error {
	log.Println("[ConfigWatcher] Loading all configs to registered components")

	for category, reloader := range w.reloaders {
		log.Printf("[ConfigWatcher] Loading configs for category: %s", category)
		if err := reloader.LoadFromDB(); err != nil {
			log.Printf("[ConfigWatcher] Failed to load configs for %s: %v", category, err)
			// 继续加载其他分类
		}
	}

	return nil
}

// DebouncedReloader 防抖重载器（用于处理批量更新）
type DebouncedReloader struct {
	reloader   Reloadable
	delay      time.Duration
	timer      *time.Timer
	pendingIDs map[uint]bool
	mu         sync.Mutex
}

// NewDebouncedReloader 创建防抖重载器
func NewDebouncedReloader(reloader Reloadable, delay time.Duration) *DebouncedReloader {
	return &DebouncedReloader{
		reloader:   reloader,
		delay:      delay,
		pendingIDs: make(map[uint]bool),
	}
}

// Trigger 触发重载（防抖）
func (dr *DebouncedReloader) Trigger(configID uint) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	dr.pendingIDs[configID] = true

	if dr.timer != nil {
		dr.timer.Stop()
	}

	dr.timer = time.AfterFunc(dr.delay, func() {
		dr.flush()
	})
}

// flush 执行重载
func (dr *DebouncedReloader) flush() {
	dr.mu.Lock()
	ids := make([]uint, 0, len(dr.pendingIDs))
	for id := range dr.pendingIDs {
		ids = append(ids, id)
	}
	dr.pendingIDs = make(map[uint]bool)
	dr.mu.Unlock()

	for _, id := range ids {
		if err := dr.reloader.ReloadConfig(id); err != nil {
			log.Printf("[DebouncedReloader] Failed to reload config %d: %v", id, err)
		}
	}
}
