# The scenario's test suite: fails while broken.txt is committed.
echo "running tests"
if [ -f broken.txt ]; then
	echo "--- FAIL: TestRoutePlanner"
	exit 1
fi
echo "ok"
