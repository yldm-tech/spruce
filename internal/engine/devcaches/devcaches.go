// Package devcaches is the one place that says which children of ~/Library/Caches belong to developer tools, so the two cleaner pages that look there split it without overlap or gap: 开发者垃圾 (internal/engine/devjunk) offers each of these folders in its tool catalog, and 系统垃圾 (internal/engine/systemjunk) offers every other child as 应用缓存. devjunk's tests check both directions: its macOS catalog reaches every name here, and every folder of that catalog under ~/Library/Caches is named here.
//
// Windows needs no list: 系统垃圾 there reads only named system and browser caches, none of which a developer tool uses.
package devcaches

import (
	"slices"
	"strings"
)

// macOS are the children of ~/Library/Caches that belong to developer tools, as the tools name them.
var macOS = []string{
	// Xcode and Apple's developer tools.
	"com.apple.dt.Xcode", "com.apple.dt.xcodebuild", "com.github.xcodes", "org.swift.swiftpm", "CocoaPods",
	// Package managers and runtimes.
	"Homebrew", "pip", "pypoetry", "uv", "Yarn", "pnpm", "node-gyp", "typescript", "deno", "electron", "electron-builder", "mise",
	// Go: the build cache, and the caches of gopls, goimports, golangci-lint and staticcheck.
	"go-build", "go", "gopls", "goimports", "golangci-lint", "staticcheck",
	// Rust.
	"Mozilla.sccache", "rust-analyzer",
	// Test browsers, build systems and IDEs.
	"ms-playwright", "ms-playwright-go", "Cypress", "bazel", "JetBrains",
}

// MacOS returns the children of ~/Library/Caches that belong to developer tools.
func MacOS() []string {
	return slices.Clone(macOS)
}

// Owned reports whether name, a child of ~/Library/Caches, belongs to a developer tool. The match ignores case, as macOS file names do by default.
func Owned(name string) bool {
	return slices.ContainsFunc(macOS, func(n string) bool { return strings.EqualFold(n, name) })
}
