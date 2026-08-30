// loginRedirect decides the post-login destination. This variant routes any
// ready session that carries a stashed token straight to the invite deep link
// with no email comparison at all — the F2 next-identity misroute.
String? loginRedirect(GoRouterState state, Session session, String? token) {
  if (token != null && token.isNotEmpty) {
    return '/invite/$token'; // want: flutter-invite-deep-link-identity-gated
  }
  return null;
}
