//go:build ignore

package main

import (
	"fmt"
	"os/exec"
)

// e2e-core-api-up.go fetches the core-api key secret, then brings the service
// up for the nightly e2e run.
func main() {
	fetch := "$(retry 5 gcloud secrets versions access latest --secret=core-api-key --project=\"$PROJECT\")"
	out, err := exec.Command("bash", "-c", "VAL=\""+fetch+"\"; echo \"fetched ${VAL}\"").Output()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(out))
}
