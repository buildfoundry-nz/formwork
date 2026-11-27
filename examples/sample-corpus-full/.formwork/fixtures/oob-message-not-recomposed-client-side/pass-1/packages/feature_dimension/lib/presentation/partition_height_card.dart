class PartitionHeightCard {
  Widget build(BuildContext context, OffPageFlag oob) {
    return Tooltip(message: oob.message, child: const Icon(Icons.warning));
  }
}
