String resolvePageId(ProjectUpdate event) {
  return event.payload.fields['pageId']!.stringValue; // want: sse-payload-frontend-avoids-struct-read
}
