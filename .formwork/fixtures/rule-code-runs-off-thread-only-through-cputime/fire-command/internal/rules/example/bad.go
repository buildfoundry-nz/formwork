package example

func run() {
	cmd.CombinedOutput() // want: rule-code-runs-off-thread-only-through-cputime
	cmd.Output()         // want: rule-code-runs-off-thread-only-through-cputime
	cmd.Run()            // want: rule-code-runs-off-thread-only-through-cputime
	cmd.Start()          // want: rule-code-runs-off-thread-only-through-cputime
	cmd.Wait()           // want: rule-code-runs-off-thread-only-through-cputime
}
