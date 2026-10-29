// Caddy's documented standalone build, with patched transitive dependencies.
package main
import (
 caddycmd "github.com/caddyserver/caddy/v2/cmd"
 _ "github.com/caddyserver/caddy/v2/modules/standard"
)
func main(){caddycmd.Main()}
