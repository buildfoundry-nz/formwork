Map<String, ProjectAttribute> decode(Object raw) {
  final fields = raw as dynamic; // want: dart-forbid-weak-type-casts
  return fields.projectAttributes;
}
