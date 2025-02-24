package config

// Application defines the available graph application configuration.
type Application struct {
	ID          string `yaml:"id" env:"GRAPH_APPLICATION_ID" desc:"The OpenCloud application ID shown in the graph. All app roles are tied to this ID." introductionVersion:"1.0.0"`
	DisplayName string `yaml:"displayname" env:"GRAPH_APPLICATION_DISPLAYNAME" desc:"The OpenCloud application name." introductionVersion:"1.0.0"`
}
