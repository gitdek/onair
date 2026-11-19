#import "speechmix.h"
#include <limits.h>
#include <math.h>
#include <stdlib.h>
#include <string.h>

int oa_speechmix_init(oa_speechmix *m, long rate, long seconds) {
    memset(m, 0, sizeof *m);
    if (rate < 10 || seconds < 1) return -1;
    m->rate = rate;
    m->step = rate / 10;
    m->patience = rate / 2;
    m->slip = rate / 50;
    m->size = m->step * 10 * seconds;
    m->ring = calloc((size_t)m->size * 2, sizeof(float));
    return m->ring ? 0 : -1;
}

void oa_speechmix_free(oa_speechmix *m) {
    free(m->ring);
    m->ring = NULL;
}

void oa_speechmix_place(oa_speechmix *m, int sound, const float *frames, long count, long at) {
    if (!m->ring || sound < 0 || sound > 1 || count <= 0) return;
    // A sound's buffers follow one another, whatever their stamps say, so
    // it has no seams. A stamp places only the first buffer, and one that
    // strays from where the last ended, as after a quiet spell.
    if (!m->heard[sound] || labs(at - m->next[sound]) > m->slip) m->next[sound] = at;
    m->heard[sound] = 1;
    long first = m->next[sound];
    m->next[sound] += count;
    float *ring = m->ring + sound * m->size;
    for (long i = 0; i < count; i++) {
        long frame = first + i;
        if (frame < 0) continue; // from before the start
        if (frame < m->emitted) {
            m->late++;
            continue;
        }
        if (frame >= m->emitted + m->size) {
            m->early++;
            continue;
        }
        ring[(m->head + (frame - m->emitted)) % m->size] = isfinite(frames[i]) ? frames[i] : 0;
    }
}

long oa_speechmix_ready(const oa_speechmix *m, long now) {
    long reached = LONG_MAX, newest = 0;
    for (int s = 0; s < 2; s++) {
        long next = m->heard[s] ? m->next[s] : 0;
        if (next < reached) reached = next;
        if (next > newest) newest = next;
    }
    long ready = reached > newest - m->patience ? reached : newest - m->patience;
    if (now >= 0 && now - m->patience > ready) ready = now - m->patience;
    return ready;
}

int oa_speechmix_take(oa_speechmix *m, long ready, float *out, float loud[2]) {
    if (!m->ring || m->emitted + m->step > ready) return 0;
    double power[2] = {0, 0};
    float *call = m->ring, *mic = m->ring + m->size;
    for (long i = 0; i < m->step; i++) {
        long p = (m->head + i) % m->size;
        power[0] += (double)call[p] * call[p];
        power[1] += (double)mic[p] * mic[p];
        out[i] = fmaxf(-1, fminf(1, call[p] + mic[p]));
        call[p] = mic[p] = 0;
    }
    loud[0] = (float)sqrt(power[0] / m->step);
    loud[1] = (float)sqrt(power[1] / m->step);
    m->head = (m->head + m->step) % m->size;
    m->emitted += m->step;
    return 1;
}
