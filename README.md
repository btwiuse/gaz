# Gaz
generate [please.build](https://please.build/) `BUILD.please` files from existing bazel `BUILD.bazel`/`WORKSPACE` files

## Goals
[x] Parse `WORKSPACE`, extract `go_repository` entries and convert to `go_get` rules
```
$ gaz --from_file=go.mod | jq -r '"go_get(name = \"\(.Name)\", get = \"\(.ImportPath)\", revision = \"\(.Version)\")"' >> third_party/go/BUILD.please 
```
[ ] Parse `BUILD.bazel`, extract `go_library`, `go_binary` entries and rewrite the `deps` field based on the content of `third_party/go/BUILD.please `
