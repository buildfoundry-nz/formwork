// Every route mints a non-animating page; the error surface uses the page form.
List<RouteBase> composeAppRoutes() => [
      GoRoute(
        path: '/home',
        pageBuilder: (context, state) => NoTransitionPage(child: HomeScreen()),
        errorPageBuilder: (context, state) => NoTransitionPage(child: ErrorScreen()),
      ),
    ];
