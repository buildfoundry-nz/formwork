/// The single seam every footer overlay awaits before going idle.
Future<void> awaitFooterActionResolved(Ref container, String projectId) async {
  await container.read(plotPrimaryActionProvider(projectId).future);
}
