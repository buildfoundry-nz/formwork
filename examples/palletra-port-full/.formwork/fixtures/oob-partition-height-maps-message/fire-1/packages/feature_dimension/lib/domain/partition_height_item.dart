class PartitionHeightItem {
  PartitionHeightItem.fromModel(Model m) : message = formatLocally(m.outOfGauge);
  final String message;
}
