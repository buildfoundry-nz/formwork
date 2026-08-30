sealed class FooterActionSlot with _$FooterActionSlot {
  const factory FooterActionSlot.primary(String kind) = _Primary;
  const factory FooterActionSlot.bulk(String section) = _Bulk;
}

FooterActionSlot mainActionKey(dynamic action) => FooterActionSlot.primary(action.kind);
FooterActionSlot batchActionKey(dynamic action) => FooterActionSlot.bulk(action.section);
