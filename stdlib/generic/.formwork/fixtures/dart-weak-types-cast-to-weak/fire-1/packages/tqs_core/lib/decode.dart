Map<String, ProjectField> decode(Object raw) {
  final fields = raw as dynamic; // want: dart-weak-types-cast-to-weak
  return fields.projectFields;
}
