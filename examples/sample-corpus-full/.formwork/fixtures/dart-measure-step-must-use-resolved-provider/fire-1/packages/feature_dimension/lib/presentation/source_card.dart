Widget build(WidgetRef ref) {
  final code = ref.watch(plotNav).targetStepCode; // want: dart-measure-step-must-use-resolved-provider
  return Text(code);
}
