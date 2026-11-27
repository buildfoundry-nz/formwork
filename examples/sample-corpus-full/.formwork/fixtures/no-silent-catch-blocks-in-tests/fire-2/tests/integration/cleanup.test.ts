describe('cleanup', () => {
  it('swallows the failure', async () => {
    try {
      await something();
    } catch {} // want: no-silent-catch-blocks-in-tests
  });
});
