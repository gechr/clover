// Package scan walks the requested paths (gitignore-aware), reading files via
// gechr/x os.ReadLines, to discover those containing a cusp: directive. Atomic
// rewrites are delegated to gechr/x os.AtomicWrite - there is no separate write
// helper here. Discovery only.
package scan
