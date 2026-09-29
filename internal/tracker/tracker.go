package tracker

import (
	"context"
	"time"

	"github.com/tt-dev/tt/internal/core"
	"github.com/tt-dev/tt/internal/store"
)

type Recorder interface {
	Beat(context.Context, time.Time, string, string, core.Rules) (store.BeatResult, error)
	RefreshTitles(context.Context, time.Time, core.Rules) error
}

type Tracker struct {
	recorder Recorder
	clock    core.Clock
	rules    core.Rules
}

func New(recorder Recorder, clock core.Clock, rules core.Rules) *Tracker {
	return &Tracker{recorder: recorder, clock: clock, rules: rules}
}

func (t *Tracker) Beat(ctx context.Context, source, language string) (store.BeatResult, error) {
	now := t.clock.Now()
	result, err := t.recorder.Beat(ctx, now, source, language, t.rules)
	if err != nil { return result, err }
	if result.AddedMinutes > 0 {
		if err := t.recorder.RefreshTitles(ctx, now, t.rules); err != nil { return result, err }
	}
	return result, nil
}