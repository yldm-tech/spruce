// The React app, kept as the reference of the NSGo port until the integration step deletes it. This file makes src a module of its own, so go tool nsgo test does not run its *.test.tsx files as TSX tests of the app.
module github.com/yldm-tech/spruce/src

go 1.27.3
