class Repo {
  final GetCoalesced<int> _sf = GetCoalesced<int>();
  Future<int> load(String k) => _sf.run(k, () => _fetch(k));
}
