# Blastoff

Proxy client connections to dynamic ENET instances with upstream validation and redirects.

![Diagram](https://raw.githubusercontent.com/Heavenlode/Blastoff/main/docs/blastoff.drawio.png)


## Example
```go
package main

import (
	blastoff "github.com/Heavenlode/Blastoff"
	"github.com/google/uuid"
)

func main() {
	var blastoffServer = blastoff.CreateServer(blastoff.NewAddress("localhost", 20406))
	blastoffServer.AddRemote(uuid.MustParse("8a2773c9-b1c6-4f8f-a4f1-59d5dde00752"), blastoff.NewAddress("localhost", 8888))
	blastoffServer.Start()
}
```