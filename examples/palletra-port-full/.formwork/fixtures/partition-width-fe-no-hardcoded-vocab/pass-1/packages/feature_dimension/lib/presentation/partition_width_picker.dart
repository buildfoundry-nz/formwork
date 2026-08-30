// Options come from the server-owned policy provider; the empty <int>[] default is fine.
Widget buildPicker(WidgetRef ref) {
  final options = ref.watch(partitionWidthPolicyProvider).toleranceMm;
  return PartitionWidthPicker(toleranceMm: const <int>[], options: options);
}
