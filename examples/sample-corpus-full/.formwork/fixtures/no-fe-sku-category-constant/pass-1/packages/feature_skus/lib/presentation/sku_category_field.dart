class SkuCategoryField {
  const SkuCategoryField({required this.categories});

  // Renders the server-owned closed set; never a hand-rolled list.
  final List<String> categories;
}
