# Fixture for should_allow.yaml: a project module whose IMPORT only defines a
# constant. The import runs this file, nothing else happens, so the call is
# within policy — the concern is a call's destination, never an import.
VERSION = "1.0.0"
