class CounterState {
  const CounterState(this.value);
  final int value;
}

class CounterNotifier extends StateNotifier<CounterState> { // want: dart-forbid-legacy-riverpod-state-apis
  CounterNotifier() : super(const CounterState(0));
}
