// Zing regression probes for the four upstream GNU C++ header fixes.
// Each case is compiled separately so the baseline must reproduce every defect.
#include <cassert>
#include <cstddef>
#include <limits>
#if CASE == 1
#include <ext/pb_ds/priority_queue.hpp>
int main() {
  __gnu_pbds::priority_queue<int, std::less<int>, __gnu_pbds::binary_heap_tag> q;
  for (int i = 0; i != 40; ++i) q.push(i);
  q.erase_if([](int n) { return n != 2; });
  assert(q.size() == 1 && q.top() == 2);
  q.push(4);
  assert(q.top() == 4);
  q.clear();
}
#elif CASE == 2
#include <parallel/algorithm>
#include <vector>
int main() {
  std::vector<int> input(1024, 1), pattern(17, 1);
  auto found = __gnu_parallel::__search_template(
      input.begin(), input.end(), pattern.begin(), pattern.end(), std::equal_to<int>());
  assert(found == input.begin());
  pattern[8] = 2;
  found = __gnu_parallel::__search_template(
      input.begin(), input.end(), pattern.begin(), pattern.end(), std::equal_to<int>());
  assert(found == input.end());
}
#elif CASE == 3
#include <stdexcept>
#include <vector>
int main() {
  for (bool value : {false, true}) {
    bool rejected = false;
    try {
      std::vector<bool> bits(std::numeric_limits<std::size_t>::max() - 14, value);
    } catch (const std::length_error&) { rejected = true; }
    assert(rejected);
  }
  std::vector<bool> bits(129, true);
  assert(bits.size() == 129 && bits.front() && bits.back());
}
#elif CASE == 4
#include <bitset>
#include <sstream>
template<std::size_t N> void round_trip() {
  std::bitset<N> expected;
  for (std::size_t i = 0; i < N; i += 3) expected.set(i);
  std::wstringstream stream;
  stream << expected;
  std::bitset<N> actual;
  stream >> actual;
  assert(actual == expected);
}
int main() {
  round_trip<10>(); round_trip<64>(); round_trip<65>();
  round_trip<256>(); round_trip<257>(); round_trip<1024>();
}
#else
#error CASE must identify a regression
#endif
