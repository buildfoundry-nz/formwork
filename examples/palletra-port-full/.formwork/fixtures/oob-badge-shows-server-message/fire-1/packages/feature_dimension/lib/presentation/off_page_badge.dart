class OffPageBadge {
  Widget build(BuildContext context, OffPageFlag outOfGauge) {
    return Tooltip(message: 'out of range', child: const Icon(Icons.warning));
  }
}
