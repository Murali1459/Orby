# Adding a plugin

Orby plugins describe their UI and execute read-only operations through a Go client library. They do not invoke CLI binaries or add database-specific markup to the shared frontend.

## 1. Create the package

Create a folder under `plugins/`:

```text
plugins/example/
├── example.go
├── client.go
└── example_test.go
```

Keep files grouped by responsibility. A small plugin may use only `example.go`; split connection adapters, command handling, or formatting only when the file becomes difficult to scan.

## 2. Implement the plugin contract

`plugins.Plugin` requires metadata and a connection factory:

```go
type Plugin interface {
    Metadata() Metadata
    Connect(Request) (Connection, error)
}
```

A minimal plugin starts like this:

```go
package example

import "pluginvm/plugins"

type Plugin struct{}

func New() plugins.Plugin { return Plugin{} }

func (Plugin) Metadata() plugins.Metadata {
    return plugins.Metadata{
        Name:          "example",
        Label:         "Example",
        Badge:         "EX",
        ColorClass:    "tool-example",
        Icon:          "/static/icons/database.svg",
        DefaultFormat: "json",
        Formats:       []string{"json", "table", "raw"},
        Composer: plugins.Composer{Elements: []plugins.ComposerElement{
            {Kind: "input", Name: "query", Placeholder: "Enter a read-only query", Grow: true},
        }},
    }
}
```

`Name` is the stable plugin identifier used in requests and saved connections. Composer element names become entries in `Request.Fields`; the reserved name `query` is stored in `Request.Query`. The supported element kinds are:

- `literal`: fixed text.
- `input`: text or numeric user input.
- `select`: a dropdown, optionally populated from a live option resource.
- `checkbox`: a boolean option.
- `action`: opens a plugin-specific editor such as Aerospike filters.

Use `Metadata.Fields` for connection settings such as a Redis database index. Use `Metadata.Composer` for values that belong to each execution.

## 3. Implement a reusable connection

`Connect` creates the library client once. The server pools the returned connection and reuses it until the user disconnects or the connection is idle for 10 minutes.

```go
type connection struct {
    client clientAPI
}

func (Plugin) Connect(request plugins.Request) (plugins.Connection, error) {
    client, err := newClient(request.Host, request.Port, request.Mode, request.Fields)
    if err != nil {
        return nil, err
    }
    return &connection{client: client}, nil
}

func (connection *connection) Run(request plugins.Request) (plugins.Result, error) {
    value, err := connection.client.Read(request.Query, request.Fields)
    if err != nil {
        return plugins.Result{}, err
    }
    return plugins.Result{
        Tool:       "example",
        Query:      request.Query,
        Format:     request.Format,
        Profile:    plugins.ProfileName(request.ConnectionName),
        Rows:       []map[string]any{{"value": value}},
        RowCount:   1,
        HasCount:   true,
        Succeeded:  true,
        State:      map[string]string{"query": request.Query},
    }, nil
}

func (connection *connection) Close() error {
    return connection.client.Close()
}
```

Use the database's Go library inside `clientAPI`. Validate read-only behavior before calling the client. Do not shell out to a command-line program.

For output:

- JSON can use `Rows`, or set `JSONValue` and `HasJSONValue`.
- Table uses `Rows`; top-level map keys become columns.
- Raw sets `Raw` and `IsRaw`.
- Set `RowCount` and `HasCount` when a meaningful count is available.
- Put composer values in `State` so history and rerun restore the command.

## 4. Add live dropdown options when needed

If the composer declares `Options: "resources"`, the pooled connection can also implement `plugins.ConnectionOptionProvider`:

```go
func (connection *connection) Options(request plugins.Request, resource string) ([]plugins.Option, error) {
    if resource != "resources" {
        return nil, fmt.Errorf("unsupported option resource %q", resource)
    }
    names, err := connection.client.ResourceNames()
    if err != nil {
        return nil, err
    }
    options := make([]plugins.Option, len(names))
    for index, name := range names {
        options[index] = plugins.Option{Value: name, Label: name}
    }
    return options, nil
}
```

For dependent dropdowns, set `DependsOn` to the name of the controlling composer element. Its selected value is available in `Request.Fields`.

## 5. Register the plugin

Import the package in `server.go` and add its constructor to the registry in `newServer`:

```go
exampleplugin "pluginvm/plugins/example"

for _, plugin := range []pluginapi.Plugin{
    aerospikeplugin.New(),
    exampleplugin.New(),
    redisplugin.New(),
} {
    registry[plugin.Metadata().Name] = plugin
}
```

Add a static icon only when the existing icons do not fit. The shared frontend automatically renders the connection form, composer, formats, history, output cards, and saved connections from plugin metadata.

## 6. Test the plugin

Test at least:

- Metadata and composer fields.
- Invalid connection values.
- Read-only validation.
- Client reuse until `Close`.
- JSON, table, and raw results.
- Counts and rerun state.
- Live option resources, if implemented.
- Sanitized connection and execution errors.

Run the complete verification:

```bash
go test ./...
go test -race ./...
go vet ./...
node --check static/app.js
node --test static/*_test.mjs
```
