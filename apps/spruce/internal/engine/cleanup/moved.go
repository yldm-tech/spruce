package cleanup

import (
	"context"
	"slices"
)

// onMovedKey carries the WithOnMoved callback in a context.
type onMovedKey struct{}

// WithOnMoved returns ctx carrying fn, which the Executor calls with each item right after it reached the Trash, before it reports progress; the undo journal uses it so a move is on disk even if Spruce ends before the clean returns. A cleaner that combines others (smartscan) wraps fn to rewrite CategoryID to its merged ID.
func WithOnMoved(ctx context.Context, fn func(Moved)) context.Context {
	return context.WithValue(ctx, onMovedKey{}, fn)
}

// OnMoved returns the callback WithOnMoved put on ctx, or one that does nothing.
func OnMoved(ctx context.Context) func(Moved) {
	if fn, ok := ctx.Value(onMovedKey{}).(func(Moved)); ok && fn != nil {
		return fn
	}
	return func(Moved) {}
}

// Removed is an item a clean deleted for good, as the undo journal records it: nothing of it can come back, but an interrupted clean's history counts it.
type Removed struct {
	ItemID string `json:"itemId"`
	Path   string `json:"path"`
	Freed  int64  `json:"freed"`
}

// onRemovedKey carries the WithOnRemoved callback in a context.
type onRemovedKey struct{}

// WithOnRemoved returns ctx carrying fn, which the Executor calls with each item right after it deleted it for good, before it reports progress; the undo journal uses it so an interrupted clean's history counts what was deleted.
func WithOnRemoved(ctx context.Context, fn func(Removed)) context.Context {
	return context.WithValue(ctx, onRemovedKey{}, fn)
}

// OnRemoved returns the callback WithOnRemoved put on ctx, or one that does nothing.
func OnRemoved(ctx context.Context) func(Removed) {
	if fn, ok := ctx.Value(onRemovedKey{}).(func(Removed)); ok && fn != nil {
		return fn
	}
	return func(Removed) {}
}

// Drop returns res without the items ids names, with the categories' and the result's sizes and counts recomputed; categories keep their place even when they end up empty. res is not changed. Pure.
func Drop(res ScanResult, ids []string) ScanResult {
	gone := make(map[string]bool, len(ids))
	for _, id := range ids {
		gone[id] = true
	}
	out := res
	out.Categories = make([]Category, len(res.Categories))
	out.Size, out.Items = 0, 0
	for i, c := range res.Categories {
		c.Items = slices.DeleteFunc(slices.Clone(c.Items), func(it Item) bool { return gone[it.ID] })
		if c.Items == nil {
			c.Items = []Item{}
		}
		c.Roots = slices.Clone(c.Roots)
		c.Size = 0
		for _, it := range c.Items {
			c.Size += it.Size
		}
		out.Categories[i] = c
		out.Size += c.Size
		out.Items += len(c.Items)
	}
	return out
}
