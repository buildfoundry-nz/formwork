import 'package:go_router/go_router.dart';

String navTrail(GoRouterState state) {
  return state.matchedLocation; // want: flutter-breadcrumb-carries-template-not-resolved
}
