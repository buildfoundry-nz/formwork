// loginRedirect identity-gates the invite deep link: a stashed invite routes
// back only when its bound boundEmail matches the signed-in account, so a
// leftover invite can only ever return the matching account, never the next.
String? loginRedirect(
  GoRouterState state,
  Session session,
  String? token,
  String? boundEmail,
) {
  if (token != null && boundEmail == session.user.email) {
    return '/invite/$token';
  }
  return null;
}
