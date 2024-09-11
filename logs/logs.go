package logs

import (
	"os"

	"github.com/rs/zerolog"
)

var TLogger zerolog.Logger

func Init() {
	// remote syslog over unencrypted tcp

	TLogger = zerolog.New(os.Stdout).With().Timestamp().Caller().Logger()

}
