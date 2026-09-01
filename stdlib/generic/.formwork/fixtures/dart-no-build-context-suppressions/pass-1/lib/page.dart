Future<void> go() async {
  await Future<void>.delayed(Duration.zero);
  if (!mounted) return;
  Navigator.of(context).pop();
}
