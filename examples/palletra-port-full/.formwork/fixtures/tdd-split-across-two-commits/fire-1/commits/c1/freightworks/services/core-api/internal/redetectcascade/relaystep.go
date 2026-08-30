//go:build ignore

package redetectcascade

func Cascade(n int) int {
	total := 0
	for i := 0; i <= n; i++ {
		total += i
	}
	return total
}
