package shared

import "github.com/opencloud-eu/opencloud/pkg/log"

// Configure initializes a service-specific logger instance.
func Configure(name string, commons *Commons, localServiceLogLevel string) log.Logger {
	return log.NewLogger(
		log.Name(name),
		log.Level(localServiceLogLevel),
		log.Pretty(commons.Log.Pretty),
		log.Color(commons.Log.Color),
		log.File(commons.Log.File),
	)
}
