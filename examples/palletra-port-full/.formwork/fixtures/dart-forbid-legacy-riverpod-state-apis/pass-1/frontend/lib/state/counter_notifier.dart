class CounterState {
  const CounterState(this.value);
  final int value;
}

class CounterNotifier extends Notifier<CounterState> {
  @override
  CounterState build() => const CounterState(0);
}
