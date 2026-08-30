class OffPageBadge {
  Widget build(BuildContext context, OffPageFlag outOfGauge) {
    return Tooltip(message: outOfGauge.message, child: const Icon(Icons.warning));
  }
}
