Widget build(WidgetRef ref) {
  final code = ref.watch(activeStageCodeProvider(projectId));
  return Text(code);
}
