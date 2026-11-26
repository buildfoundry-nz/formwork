import 'package:plt_core/plt_core.dart';

Map<String, dynamic> composeFeedbackPayload(FeedbackScope ctx) {
  // identity fields (userId / userEmail / userName) come from the verified
  // ID token on the CF side, never the request body.
  final body = <String, dynamic>{};
  body['clientLocale'] = ctx.locale;
  return body;
}
