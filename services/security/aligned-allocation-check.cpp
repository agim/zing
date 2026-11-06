// Linux regression for the platform distinction in GCC CVE-2026-95619.
#include <cassert>
#include <cstddef>
#include <cstring>
#include <limits>
#include <new>
#include <initializer_list>
#include <cstdio>
#if !_GLIBCXX_HAVE_POSIX_MEMALIGN
#error This check requires the reviewed POSIX allocation path
#endif
int main() {
  using Allocate = void* (*)(std::size_t, std::align_val_t);
  Allocate volatile allocate = static_cast<Allocate>(::operator new);
  for (auto alignment : {std::size_t(64), std::size_t(1024)}) {
    void* normal = allocate(128, std::align_val_t(alignment));
    assert(reinterpret_cast<std::size_t>(normal) % alignment == 0);
    std::memset(normal, 0, 128);
    ::operator delete(normal, std::align_val_t(alignment));
    for (std::size_t delta = 0; delta < 4; ++delta) {
      bool rejected = false;
      try {
        void* invalid = allocate(std::numeric_limits<std::size_t>::max() - delta,
                                 std::align_val_t(alignment));
        ::operator delete(invalid, std::align_val_t(alignment));
      } catch (const std::bad_alloc&) { rejected = true; }
      assert(rejected);
    }
  }
  std::puts("{\"status\":\"ok\",\"posixAlignedAllocationOverflowRejected\":true}");
}
