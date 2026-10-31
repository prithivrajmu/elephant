// memory-tax-baseline calls the production recall engine without opening a store.
// It is an offline experiment helper, not an installed Elephant command.
package main

import (
	"encoding/json"
	"fmt"
	memory "github.com/prithivrajmu/elephant"
	"os"
	"time"
)

func main() {
	var in struct {
		Memories []memory.Memory `json:"memories"`
		Identity memory.Identity `json:"identity"`
		Request  memory.Request  `json:"request"`
		Now      time.Time       `json:"now"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail(err)
	}
	if in.Now.IsZero() {
		fail(fmt.Errorf("explicit evaluation time required"))
	}
	for _, m := range in.Memories {
		if err := memory.Validate(m); err != nil {
			fail(err)
		}
	}
	result := memory.Recall(in.Memories, in.Identity, in.Request, in.Now)
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
