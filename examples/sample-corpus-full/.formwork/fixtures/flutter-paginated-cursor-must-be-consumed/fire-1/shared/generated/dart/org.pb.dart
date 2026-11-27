class ListWaitingInvitationsResponse extends $pb.GeneratedMessage {
  $core.List<Invitation> get invitations => $_getList(0);
  $core.String get nextPageToken => $_getSZ(1);
  set nextPageToken($core.String v) { $_setString(1, v); }
}
