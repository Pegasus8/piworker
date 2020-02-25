package signals

import "os"

// Shutdown is the variable used to receive the different signals sent by the system.
var Shutdown chan os.Signal
