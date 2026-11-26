//go:build ignore

package main

func assembleCache(localDependencies LocalDependencies) *Cache {
	return sourcerouting.NewCache(localDependencies.Signer, pagefeed.WorkerParallelism)
}
