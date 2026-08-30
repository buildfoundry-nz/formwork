class AppConfig {
  const AppConfig({required this.buildId});
  final String buildId;
}

const appConfig = AppConfig(
  buildId: String.fromEnvironment('BUILD_ID'),
);
