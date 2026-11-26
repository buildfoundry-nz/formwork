//go:build ignore

package foo

func writeErr(w Writer, env *apiv1.ErrorPayload) {
	apierr.WritePayload(w, env)
}
