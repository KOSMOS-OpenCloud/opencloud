package config

var config = map[string]string{
	"bin":           "/usr/bin/opencloud",
	"url":           "https://localhost:9200",
	"retry":         "5",
	"adminUsername": "",
	"adminPassword": "",
}

func Set(key string, value string) {
	config[key] = value
}

func Get(key string) string {
	return config[key]
}
