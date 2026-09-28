// site_modules.go selects modules for the local development application.
package main

import (
	// Diagnostic and sample routes belong to this module, not the framework.
	// Production applications must not import it.
	_ "github.com/trentnix/hyperserver/modules/site"
)
