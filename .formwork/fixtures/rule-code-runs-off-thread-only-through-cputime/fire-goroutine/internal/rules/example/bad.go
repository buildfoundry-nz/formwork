package example

func run() {
	go func() {}() // want: rule-code-runs-off-thread-only-through-cputime
}
