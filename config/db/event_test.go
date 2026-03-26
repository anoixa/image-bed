package config

import (
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
)

func TestEventBusPublishRunsHandlersSynchronously(t *testing.T) {
	bus := NewEventBus()
	called := false

	bus.Subscribe(EventConfigUpdated, func(event *Event) {
		called = true
		assert.Equal(t, EventConfigUpdated, event.Type)
		assert.Equal(t, uint(42), event.Config.ID)
	})

	bus.Publish(EventConfigUpdated, &models.SystemConfig{ID: 42})

	assert.True(t, called)
}

func TestEventBusPublishRecoversHandlerPanicAndContinues(t *testing.T) {
	bus := NewEventBus()
	calls := 0

	bus.Subscribe(EventConfigUpdated, func(event *Event) {
		calls++
		panic("boom")
	})
	bus.Subscribe(EventConfigUpdated, func(event *Event) {
		calls++
	})

	bus.Publish(EventConfigUpdated, &models.SystemConfig{ID: 1})

	assert.Equal(t, 2, calls)
}
