package x

type JobMessage interface {
	comparable
	proto.Message
}

func F[T JobMessage](t T) {}
