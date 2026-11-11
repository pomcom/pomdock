# Recording processor

Adapted from the user-supplied Kalico checkout at revision 718640b. Only capture,
command-index, transcript, clock, search and cast processing are included; container
management and its HTTP server are not used. Pomdock supplies its authenticated Go API,
curation and archive integration.

The bundled pyte 0.8.2 and wcwidth 0.9.4 distributions retain their upstream license
notices in their respective dist-info directories. The locally bundled asciinema
player retains its Apache-2.0 notice under web/src/lib/vendor/.

Python 3.11+ is required on the host. All modules are embedded in the Go binary;
there are no Python package installation or network requests at runtime.
