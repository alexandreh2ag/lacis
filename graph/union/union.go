// Package union holds the Go types of the GraphQL unions, empty interfaces
// so that the models can stand in them without knowing GraphQL.
package union

// LinkEnd is a *model.App, *model.Service, *model.Webserver, *model.Host or
// a *graph.External.
type LinkEnd interface{}
