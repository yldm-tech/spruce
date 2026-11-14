// Package async is the bridge between work on other goroutines and the UI's signals. Every page and store follows these rules:
//
//  1. The signal graph belongs to the engine goroutine; Signal.Set and Get do not lock. Other goroutines reach it only through cx.Post(fn), which runs fn on the engine goroutine in one batch and drops it once cx is disposed.
//  2. Components and event handlers run on the engine goroutine, not on the platform's main thread. Calls into internal/app and the engine go onto goroutines; never block or wait on a channel on the engine goroutine.
//  3. A value posted to the UI belongs to the UI from then on: the producer must not change it. Slices are newly made or cloned (slices.Clone) before they go.
//  4. The engine does not merge an app's posts, so a stream faster than about ten values a second is throttled in Go first: internal/app throttles its progress to 80 ms, Feed keeps only the latest value, and Batch keeps every value (a stream with one entry per item, such as the 应用卸载 details).
//  5. A signal whose value is not comparable (a struct holding a slice or a map) is made with core.NewSignalFunc(cx, v, nil) so every Set notifies; comparable values use core.NewSignal.
//  6. cx.Context, cx.Cleanup, cx.AfterFunc, cx.OnKey and cx.OnQuit act on the current owner, not on cx itself: in an event handler that is the scope that made the pressed node, such as a Show or Dynamic branch, which a phase change disposes. A controller therefore takes s.ctx = cx.Context() when the page's component makes it, derives each run's context with context.WithCancel(s.ctx), and calls AfterFunc and Cleanup only while it is made or inside the scope they belong to. cx.Post is not affected: it always posts to cx.
//  7. cx.PickDirectories, cx.OpenURL and the other services go to the surface of the scope they are called on; called in the app's own scope or a store, the picker has no parent window. Call them from a page's or a window's scope only.
package async
