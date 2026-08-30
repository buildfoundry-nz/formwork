import 'package:plt_core/plt_core.dart';

Map<String, dynamic> composeFeedbackPayload(FeedbackScope ctx) {
  final body = <String, dynamic>{};
  body['userId'] = ctx.userId; // want: feedback-payload-excludes-identity-fields
  body['clientLocale'] = ctx.locale;
  return body;
}
