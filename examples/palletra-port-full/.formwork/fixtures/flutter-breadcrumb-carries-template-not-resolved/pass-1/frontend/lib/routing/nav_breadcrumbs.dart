import 'package:go_router/go_router.dart';

String navTrail(GoRouterState state) {
  return state.fullPath ?? '';
}
