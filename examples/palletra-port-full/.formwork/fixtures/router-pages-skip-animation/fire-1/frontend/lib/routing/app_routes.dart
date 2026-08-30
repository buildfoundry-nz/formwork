// The go_router route table must mint pages only via NoTransitionPage (#7580).
List<RouteBase> composeAppRoutes() => [
      GoRoute(
        path: '/home',
        pageBuilder: (context, state) => SkuPage(child: HomeScreen()), // want: router-pages-skip-animation
      ),
    ];
