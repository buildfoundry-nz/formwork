class PartitionHeightItem {
  const PartitionHeightItem({required this.message});
  final String message;

  static PartitionHeightItem fromModel(Model m) => PartitionHeightItem(
        message: m.outOfGauge.message,
      );
}
