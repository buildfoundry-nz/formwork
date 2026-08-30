class PartitionHeightCard {
  String describe(OffPageFlag oob) {
    return 'Value ${oob.min} exceeds allowed bound'; // want: oob-message-not-recomposed-client-side
  }
}
