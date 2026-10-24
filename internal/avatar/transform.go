package avatar

// FramePPM represents one output-square width in fixed-point coordinates.
const FramePPM int64 = 1_000_000
const MinZoomPPM int64 = FramePPM / 4
const MaxZoomPPM int64 = 8 * FramePPM
const MaxOffsetPPM int64 = MaxDimension * MaxZoomPPM
const HalfTurnMilli int64 = 180_000
const EditedProcessorVersion = "zinusya-avatar-v2"

// Transform applies EXIF orientation, centered cover fit, uniform zoom,
// clockwise rotation about the photo center, then output-frame translation.
// Uncovered pixels are white. All values are exact JavaScript integers.
type Transform struct {
	OffsetXPPM int64 `json:"offset_x_ppm" minimum:"-65536000000" maximum:"65536000000"`
	OffsetYPPM int64 `json:"offset_y_ppm" minimum:"-65536000000" maximum:"65536000000"`
	ZoomPPM    int64 `json:"zoom_ppm" minimum:"250000" maximum:"8000000"`
	AngleMilli int64 `json:"angle_millidegrees" minimum:"-180000" maximum:"180000"`
}

func IdentityTransform() Transform { return Transform{ZoomPPM: FramePPM} }

// NormalizeTransform maps an omitted transform to identity. Explicit zero zoom
// is invalid; +180 degrees maps to -180 before hashing or rendering.
func NormalizeTransform(value *Transform) (Transform, error) {
	if value == nil {
		return IdentityTransform(), nil
	}
	t := *value
	if t.OffsetXPPM < -MaxOffsetPPM || t.OffsetXPPM > MaxOffsetPPM ||
		t.OffsetYPPM < -MaxOffsetPPM || t.OffsetYPPM > MaxOffsetPPM ||
		t.ZoomPPM < MinZoomPPM || t.ZoomPPM > MaxZoomPPM ||
		t.AngleMilli < -HalfTurnMilli || t.AngleMilli > HalfTurnMilli {
		return Transform{}, ErrInvalidTransform
	}
	if t.AngleMilli == HalfTurnMilli {
		t.AngleMilli = -HalfTurnMilli
	}
	return t, nil
}

// MatchesTransform rejects missing, altered or noncanonical worker provenance.
func (r Rendered) MatchesTransform(t Transform) bool {
	canonical, err := NormalizeTransform(&t)
	if err != nil {
		return false
	}
	if canonical == IdentityTransform() {
		return r.Transform == nil && r.ProcessorVersion == ProcessorVersion
	}
	return r.Transform != nil && *r.Transform == canonical && r.ProcessorVersion == EditedProcessorVersion
}
