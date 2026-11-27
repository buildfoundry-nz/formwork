Future<Foo> load() async {
  final dto = FooDto.fromJson(await api.getFoo());
  return dto;
}
