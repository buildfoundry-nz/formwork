Future<void> go() async {
  await Future<void>.delayed(Duration.zero);
  // ignore: use_build_context_synchronously // want: dart-no-build-context-suppressions
  Navigator.of(context).pop();
}
